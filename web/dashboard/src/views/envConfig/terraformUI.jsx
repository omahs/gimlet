import Form from '@rjsf/core'
import validator from '@rjsf/validator-ajv8'; // TODO the validator
import { useState } from 'react';

const TerraformUI = (props) => {
  const { schema, values } = props;
  const [data, setData] = useState(values)

  // const uiSchema = {
  //   "ui:widget": (props) => {
  //     console.log("widget")
  //     console.log(props)
  //     console.log(props.value)
  //     return (<div className="grid">
  //       {props.value.map(item => {
  //         return (<div className="mb-2 items-center">
  //           <label htmlFor={item.name} className="text-gray-600 mr-4 block text-sm font-medium">
  //             {item.name}*
  //           </label>
  //           <span className="text-gray-600 mt-1 block text-sm font-light">{item.description}</span>
  //           {item.type === "string" && <input
  //             type="text"
  //             name={item.name}
  //             id={item.name}
  //             value={item.default}
  //             onChange={(event) => props.onChange()}
  //             className="mt-1 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 border-gray-300 rounded-md w-4/12"
  //           />}
  //           {item.type === "number" && <input
  //             type="number"
  //             name={item.name}
  //             id={item.name}
  //             value={item.default}
  //             onChange={(event) => props.onChange()}
  //             className="mt-1 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 border-gray-300 rounded-md w-4/12"
  //           />}
  //           {item.type === "bool" && <CustomCheckbox />}
  //         </div>)
  //       })}
  //     </div>)
  //   }
  // };

  const uiSchema = {
    "ui:options": {
      orderable: false,
      addable: false,
      removable: false,
    },
    items: {
      name: {
        "ui:autofocus": false,
        "ui:widget": "myCustomTitle",
      },
      type: {
        "ui:widget": "myCustomDesciption",
      },
      description: {
        "ui:widget": "myCustomDesciption",
      },
      "default": {
        "ui:widget": "text" // TODO
      },
      required: {
        'ui:widget': "checkbox",
      }
    },
  };

  return (
    <div>
      <div className>
        <div className="space-y-6 sm:px-6 lg:px-0">
          <Form
            key="terraform"
            onChange={e => setData(e.formData)}
            schema={schema}
            // onChange={log("changed")}
            // onSubmit={log("submitted")}
            // onError={log("errors")}
            uiSchema={uiSchema}
            formData={data}
            // fields={customFields}
            widgets={customWidgets}
            // FieldTemplate={CustomFieldTemplate}
            // className={styles('m-8')}
            liveValidate={true} // TODO validate will come from props
            validator={validator}
          />
        </div>
        <button
          type="button"
          className="bg-green-600 hover:bg-green-500 focus:outline-none focus:border-green-700 focus:shadow-outline-indigo active:bg-green-700 inline-flex items-center px-6 py-2 border border-transparent text-base leading-6 font-medium rounded-md text-white transition ease-in-out duration-150"
          onClick={() => console.log(data)}
        >
          Log data
        </button>
      </div>
    </div>
  )
};

const CustomCheckbox = (props) => {
  const { value, label } = props
  const translate = value ? 'translate-x-5' : 'translate-x-0'
  const bg = value ? 'bg-indigo-600' : 'bg-gray-200'
  return (
    <div>
      <label class="control-label">{label}</label>
      <span
        role="checkbox" tabindex="0"
        aria-checked={value}
        onClick={(event) => props.onChange(!value)}
        className={`${bg} mt-1 relative inline-flex flex-shrink-0 h-6 w-11 border-2 border-transparent rounded-full cursor-pointer transition-colors ease-in-out duration-200 focus:outline-none focus:shadow-outline`}>
        <span
          aria-hidden="true"
          className={`${translate} inline-block h-5 w-5 rounded-full bg-white shadow transform transition ease-in-out duration-200`}></span>
      </span>
    </div>
  )
}

const CustomTitle = (props) => {
  const { value } = props
  return (
    <label className="block text-sm font-medium leading-5 text-gray-700">
      {value}
    </label>
  )
}

const CustomDescription = (props) => {
  const { value } = props
  return (
    <span className="text-gray-600 mt-1 block text-sm font-light">{value}</span>
  )
}

const customWidgets = {
  CheckboxWidget: CustomCheckbox,
  myCustomTitle: CustomTitle,
  myCustomDesciption: CustomDescription,
}

export default TerraformUI;
